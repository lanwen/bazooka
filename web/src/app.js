"use strict";

angular.module('bzk', ['bzk.project', 'bzk.utils', 'bzk.templates', 'ngRoute']);

angular.module('bzk').config(function($routeProvider){
	$routeProvider
	.when('/', {
		templateUrl: 'nothing.html'
	}).otherwise({
		redirectTo: '/'
	});
});

angular.module('bzk').controller('RootController', function($scope){
	
});

angular.module('bzk').factory('ProjectsResource', function($http){
	return {
		fetch: function () {
			return $http.get('/api/project');
		},
		create: function(proj) {
			return $http.post('/api/project', proj);
		}
	};
});

angular.module('bzk').controller('ProjectsController', function($scope, ProjectsResource, $routeParams){
	
	function refresh () {
		ProjectsResource.fetch().success(function(res){
			$scope.projects = res;
		});
	}

	refresh();

	$scope.newProj = {
		scm_type: 'git'
	};

	$scope.newProjectVisible = function(s) {
		$scope.showNewProject = s;
	};

	$scope.createProject = function() {
		ProjectsResource.create($scope.newProj).success(function(){
			$scope.showNewProject = false;
			$scope.newProj = {
				scm_type: 'git'
			};
			refresh();
		});
	};

	$scope.isSelected = function(p) {
		return p.id===$routeParams.pid;
	};
});



angular.module('bzk').filter('bzoffset', function(){
	return function(o, b) {
		var t = o+b;
		if (t<60) {
			return t+' secs';
		} else if (t<3600) {
			return Math.floor(t/60)+' mins';
		} else {
			return Math.floor(t/3600) + ' hours';
		}
	};
});